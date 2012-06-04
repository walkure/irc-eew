#!/usr/bin/env perl

use CGI::Carp qw(fatalsToBrowser);

# show specified EEW log data file
# walkure at kmc.gr.jp
# cf. http://eew.mizar.jp/excodeformat

use strict;
use warnings;

use Earthquake::EEW::Decoder;
use Encode;
use Data::Dumper;
use CGI;
use feature q/:5.10/;

my $ddir = '../eewlog';

my $q=new CGI;
my $fname = $q->param('data');

print "Content-Type:text/html;charset=euc-jp\n\n";

unless($fname =~ /\d+\.+/){
	print "invalid path\n";
	exit;
}

my $path = $ddir.'/'.$fname;

open(my $fh,$path) or die "Cannot open $path:$!";
my $len = sysread $fh, my($buf), 1024;
close($fh);

my $eew = Earthquake::EEW::Decoder->new();

my $d = $eew->read_data($buf);
conv_charset($d);
my $dumped = Dumper $d;

my @wd = $d->{warn_time} =~ /\d\d/og;
my @ed = $d->{eq_time}   =~ /\d\d/og;
$d->{warn_time} = "20$wd[0]/$wd[1]/$wd[2] $wd[3]:$wd[4]:$wd[5]";
$d->{eq_time} = "20$ed[0]/$ed[1]/$wd[2] $ed[3]:$ed[4]:$ed[5]";

my $summary = eew_summary($d);

print << "_HTML_";
<html><head><title>
$summary
</title></head><body>
[<a href="./">���</a>]&nbsp;
$summary
_HTML_

print "<hr><table>\n";

my($lat,$long,$center);	
foreach my $it(keys %$d){

	given($it){
		when('shindo'){
			print '<tr><td>�������</td><td>'.$d->{shindo}."</td></tr>\n";
		}
		when('magnitude'){
			print '<tr><td>�ޥ��˥��塼��</td><td>'.$d->{magnitude};
			print '('.$d->{magnitude_accurate}.')'if(defined $d->{magnitude_accurate});
			print "</td></tr>\n";
		}
		when('eq_time'){
			print '<tr><td>��������</td><td>'.$d->{eq_time}."</td></tr>\n";
		}
		when('warn_time'){
			print '<tr><td>��������</td><td>'.$d->{warn_time}."</td></tr>\n";
		}
		when('code_type'){
			print '<tr><td>��������</td><td>'.$d->{code_type}."</td></tr>\n";
		}
		when('warn_type'){
			print '<tr><td>�����о�</td><td>'.$d->{warn_type}."</td></tr>\n";
		}
		when('msg_type'){
			print '<tr><td>���μ���</td><td>'.$d->{msg_type}."</td></tr>\n";
		}
		when('section'){
			print '<tr><td>ȯ������</td><td>'.$d->{section}."</td></tr>\n";
		}
		when('center_depth'){
			print '<tr><td>�̸�����</td><td>'.$d->{center_depth}.'km';
			print '('.$d->{center_accurate}.')' if defined $d->{center_accurate};
			print "</td></tr>\n";
		}
		when('warn_num'){
			my $times = '��'.$d->{warn_num}.'��';
			$times .= '(�ǽ�)' if ($d->{NCN_type} >= 8);
			print '<tr><td>���</td><td>'.$times."</td></tr>\n";
		}
		when('center_name'){
			$center = $d->{center_name};
			show_center($lat,$long,$center,$d);
		}
		when('center_lng'){
			$long = $d->{center_lng};
			show_center($lat,$long,$center,$d);
		}
		when('center_lat'){
			$lat = $d->{center_lat};
			show_center($lat,$long,$center,$d);
		}
		when('eq_id'){
			print '<tr><td>�Ͽ�ID</td><td>'.$d->{eq_id}."</td></tr>\n";
		}
	}
}

print "</table><hr>\n";

if(defined $d->{EBI}){
	print '<table><tr><td>�ϰ�</td><td>ͽ¬����</td><td>ͽ�ۻ���</td></tr>';
	foreach my $area (keys %{$d->{EBI}} ){
		my $ebi = $d->{EBI}{$area};

		my @ar = $ebi->{time} =~ /\d\d/og;
		$ebi->{time} = "$ar[0]:$ar[1]:$ar[2]";
		$ebi->{time} = '������ã' if($ebi->{time} eq '::');

		print '<tr><td>'.$ebi->{name}.'</td><td>����'.$ebi->{shindo1};
		if($ebi->{shindo2_code} eq '//'){
			print '�ʾ�';
		}elsif($ebi->{shindo1} ne $ebi->{shindo2}){
			print '������'.$ebi->{shindo2};
		}
		print '</td><td>'.$ebi->{time}.'</td></tr>';
	}

	print '</table><hr>';
}


print "\n<pre>$dumped</pre><hr>\n";

Encode::from_to($buf,'shift_jis','euc-jp');

$buf =~ s/\x01/\[SOH\]\n/g;
$buf =~ s/\x02/\[STX\]\n/g;
$buf =~ s/\x03/\[ETX\]\n/g;

print "<pre>$buf</pre>";

print << "_HTML_";

</body></html>
_HTML_


sub show_center
{
	my($lat,$long,$center,$d) = @_;
	return unless(defined $lat && defined $long && defined $center);
	
	my $latmsg = sprintf '%0.01f',$lat;
	my $longmsg = sprintf '%0.01f',$long;
	my $acmsg = '';

	$acmsg = '('.$d->{center_accurate}.')' if defined $d->{center_accurate};

	print << "_HTML_";
<tr><td>�̱�����</td><td>
<a href="http://maps.google.com/maps?q=$latmsg,$longmsg">N$latmsg E$longmsg</a>($center) $acmsg
</td></tr>
_HTML_

}

sub eew_summary
{
	my $d = shift;
	my $times = ' ��'.$d->{'warn_num'};
	$times .= '(�ǽ�)' if $d->{NCN_type} >=8;

	my $msg;
	if($d->{'msg_type_code'} == 10){
		$msg = $d->{warn_time}.$times.'�� ('.$d->{eq_time}.'ȯ��) ���ä�';
	}else{
		my $center = sprintf('�̱�:N%0.01f/E%0.01f(%s)����%dkm',$d->{'center_lat'},$d->{'center_lng'},$d->{'center_name'},$d->{'center_depth'});
		my $magnitude = sprintf(' ����:M%.01f ����%s',$d->{'magnitude'},$d->{'shindo'});
		$msg = $d->{warn_time}.$times.'�� ('.$d->{eq_time}.'ȯ��)'.$center.$magnitude;
	}
	$msg;
}


sub conv_charset
{
	my $d = shift;
	foreach my $key(keys %$d){
		if(ref($d->{$key}) eq 'HASH'){
			conv_charset($d->{$key});
		}else{
			$d->{$key} = Encode::encode('euc-jp',$d->{$key});
		}
	}

}
