#!/usr/bin/env perl

# show specified EEW log data file
# walkure at kmc.gr.jp
# cf. http://eew.mizar.jp/excodeformat

use strict;
use warnings;

use utf8;
use Encode;
use CGI::Fast;
use File::Spec;
use File::Slurp qw(read_file);

use lib './lib/';
use Earthquake::EEW::Decoder;

binmode STDOUT, ":utf8";

my $ddir =      defined $ENV{EEW_DATA_DIR}  ? $ENV{EEW_DATA_DIR}  : '/eewlog/';
my $path_base = defined $ENV{EEW_PATH_BASE} ? $ENV{EEW_PATH_BASE} : './';

# https://github.com/perl-catalyst/FCGI/commit/7369e6b96a59b425f5b44bdf52a95387baa0e782
if(defined *FCGI::Stream::PRINT){
	my $fcgi_print = \&FCGI::Stream::PRINT;
	undef *FCGI::Stream::PRINT;
	*FCGI::Stream::PRINT = sub {
		my $stream = shift;
		my @args = map {my $i = $_ ; utf8::encode($i); $i} @_;

		&$fcgi_print($stream,@args);
	};
}

while(my $query = new CGI::Fast){
	main($query);
}


sub main
{
	my $q = shift;
	my $fname = $q->param('name');

	print "Content-Type:text/html;charset=utf-8\n\n";

	unless(defined $fname){
		print "invalid query\n";
		exit;
	}

	unless($fname =~ /\d+\.+/){
		print "invalid name\n";
		exit;
	}

	my $path = get_fn_from_eqid($ddir,$fname);
	my $eew = Earthquake::EEW::Decoder->new();
	my $data = read_file($path, { binmode => ':encoding(sjis)' });

	my $d = $eew->read_data($data);

	my @wd = $d->{warn_time} =~ /\d\d/og;
	my @ed = $d->{eq_time}   =~ /\d\d/og;
	$d->{warn_time} = "20$wd[0]/$wd[1]/$wd[2] $wd[3]:$wd[4]:$wd[5]";
	$d->{eq_time} = "20$ed[0]/$ed[1]/$wd[2] $ed[3]:$ed[4]:$ed[5]";

	my $summary = eew_summary($d);

	print << "_HTML_";
<html><head><title>
$summary
</title>
<meta name="robots" content="noindex,nofollow,noarchive" />
</head><body>
[<a href="@{[get_return_pathquery($path_base,$fname)]}">戻る</a>]&nbsp;
$summary
_HTML_

	print "<hr><table>\n";

	my($lat,$long,$center);	
	foreach my $it(sort keys %$d){
		
		if($it eq 'shindo' ){
			print '<tr><td>最大震度</td><td>'.$d->{shindo}."</td></tr>\n";
		}elsif($it eq 'magnitude'){
			print '<tr><td>マグニチュード</td><td>'.$d->{magnitude};
			print '('.$d->{magnitude_accurate}.')'if(defined $d->{magnitude_accurate});
			print "</td></tr>\n";
		}elsif($it eq 'eq_time'){
			print '<tr><td>検知日時</td><td>'.$d->{eq_time}."</td></tr>\n";
		}elsif($it eq 'warn_time'){
			print '<tr><td>通知日時</td><td>'.$d->{warn_time}."</td></tr>\n";
		}elsif($it eq 'code_type'){
			print '<tr><td>通知内容</td><td>'.$d->{code_type}."</td></tr>\n";
		}elsif($it eq 'warn_type'){
			print '<tr><td>通知対象</td><td>'.$d->{warn_type}."</td></tr>\n";
		}elsif($it eq 'msg_type'){
			print '<tr><td>通知種類</td><td>'.$d->{msg_type}."</td></tr>\n";
		}elsif($it eq 'section'){
			print '<tr><td>発信官署</td><td>'.$d->{section}."</td></tr>\n";
		}elsif($it eq 'center_depth'){
			print '<tr><td>震源深度</td><td>'.$d->{center_depth}.'km';
			print '('.$d->{center_accurate}.')' if defined $d->{center_accurate};
			print "</td></tr>\n";
		}elsif($it eq 'warn_num'){
			my $times = '第'.$d->{warn_num}.'報';
			$times .= '(最終)' if ($d->{NCN_type} >= 8);
			print '<tr><td>報数</td><td>'.$times."</td></tr>\n";
		}elsif($it eq 'center_name'){
			$center = $d->{center_name};
			show_center($lat,$long,$center,$d);
		}elsif($it eq 'center_lng'){
			$long = $d->{center_lng};
			show_center($lat,$long,$center,$d);
		}elsif($it eq 'center_lat'){
			$lat = $d->{center_lat};
			show_center($lat,$long,$center,$d);
		}elsif($it eq 'eq_id'){
			print '<tr><td>地震ID</td><td>'.$d->{eq_id}."</td></tr>\n";
		}
	}

	print "</table><hr>\n";

	if(defined $d->{EBI}){
		print '<table><tr><td>地域</td><td>予測震度</td><td>予想時刻</td></tr>';
		foreach my $area (keys %{$d->{EBI}} ){
			my $ebi = $d->{EBI}{$area};

			my @ar = $ebi->{time} =~ /\d\d/og;
			$ebi->{time} = "$ar[0]:$ar[1]:$ar[2]";
			$ebi->{time} = '既に到達' if($ebi->{time} eq '::');

			print '<tr><td>'.$ebi->{name}.'</td><td>震度'.$ebi->{shindo1};
			if($ebi->{shindo2_code} eq '//'){
				print '以上';
			}elsif($ebi->{shindo1} ne $ebi->{shindo2}){
				print '～震度'.$ebi->{shindo2};
			}
			print '</td><td>'.$ebi->{time}.'</td></tr>';
		}

		print '</table><hr>';
	}

	show_map($lat,$long);

	#Encode::from_to($data,'cp932','utf8');

	$data =~ s/\x01/\[SOH\]\n/g;
	$data =~ s/\x02/\[STX\]\n/g;
	$data =~ s/\x03/\[ETX\]\n/g;

	print "<pre>$data</pre>";

	print << "_HTML_";

</body></html>
_HTML_

}

sub show_center
{
	my($lat,$long,$center,$d) = @_;
	return unless(defined $lat && defined $long && defined $center);
	
	my $latmsg = sprintf '%0.01f',$lat;
	my $longmsg = sprintf '%0.01f',$long;
	my $acmsg = '';

	$acmsg = '('.$d->{center_accurate}.')' if defined $d->{center_accurate};

	print << "_HTML_";
<tr><td>震央位置</td><td>
<a href="http://maps.google.com/maps?q=$latmsg,$longmsg">N$latmsg E$longmsg</a>($center) $acmsg
</td></tr>
_HTML_

}

sub show_map
{
	my($lat,$long) = @_;

	my $latmsg = sprintf '%0.01f',$lat;
	my $longmsg = sprintf '%0.01f',$long;

	print << "_HTML_";
		<iframe src="https://maps.google.com/maps?output=embed&q=$latmsg,$longmsg&t=m&hl=ja&z=7"
 width="60%" height="50%" frameborder="0" style="border:0;" allowfullscreen=""></iframe>
_HTML_

}

sub eew_summary
{
	my $d = shift;
	my $times = ' 第'.$d->{'warn_num'};
	$times .= '(最終)' if $d->{NCN_type} >=8;

	my $msg;
	if($d->{'msg_type_code'} == 10){
		$msg = $d->{warn_time}.$times.'報 ('.$d->{eq_time}.'発生) 取り消し';
	}else{
		my $center = sprintf('震央:N%0.01f/E%0.01f(%s)深さ%dkm',$d->{'center_lat'},$d->{'center_lng'},$d->{'center_name'},$d->{'center_depth'});
		my $magnitude = sprintf(' 最大:M%.01f 震度%s',$d->{'magnitude'},$d->{'shindo'});
		$msg = $d->{warn_time}.$times.'報 ('.$d->{eq_time}.'発生)'.$center.$magnitude;
	}
	$msg;
}

sub get_fn_from_eqid
{
    my ($dir,$eqid) = @_;

    my $year = substr($eqid,0,4);
    my $month = substr($eqid,4,2);
    my $day = substr($eqid,6,2);

    my $fdir = File::Spec->catdir($dir,$year,$month,$day);

    my $fpath = File::Spec->catfile($fdir,$eqid);

    $fpath;
}

sub get_return_pathquery
{
	my ($base,$eqid) = @_;

    my $year = substr($eqid,0,4);
    my $month = substr($eqid,4,2);
    my $day = substr($eqid,6,2);

	"$base?year=$year&month=$month&day=$day";
}
