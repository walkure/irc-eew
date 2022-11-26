package SlackWebhookSock;

# SlackWebhookSock.pm
# walkure at 3pf.jp

use strict;
use warnings;
use Encode;
use URI;
use JSON qw/encode_json/;
use HTTP::Response;
use base qw/IO::Socket::SSL/;

sub configure
{
	my ($self, $args) = @_;
	
	my $host = $args->{PeerHost};
	my $name = '';
	
	if(ref $host eq 'HASH'){
		$name = (keys %{$args->{PeerHost}})[0];
		$host = $args->{PeerHost}{$name};
	}
	
	unless(defined $host){
		$name = (keys %$args)[0];
		$host = $args->{$name};
	}
	
	my $path = URI->new($host);
	
	return if $path->scheme() ne 'https';
	
	$args->{PeerHost} = $path->host_port();
	*$self->{path_query} = $path->path_query();
	*$self->{host} = $path->host();
	*$self->{response} = '';
	*$self->{name} = $name || $path->host();

	#$args->{Blocking} = 0;
	
	$self->SUPER::configure($args);
}

sub parse_body
{
	my($self,$body) = @_;
	*$self->{response} .= $body;
}

sub send_json
{
	my($self,$body) = @_;
	
	my $json = encode_json($body);
	my $path_query = *$self->{path_query};
	my $host = *$self->{host};
	
	#required by slack server.
	my $contentLength = length($json);

	# HTTP::Response does not parse chunked transfer.
	print $self "POST $path_query HTTP/1.0\r\nConnection:Close\r\nHost: $host\r\n"
		."Content-type: application/json; charset=utf-8\r\nContent-Length: $contentLength\r\n\r\n$json";
}

sub name
{
	my $self = shift;
	my $old = *$self->{name};
    *$self->{name} = $_[0] if @_;
    
    $old;
}

sub get_response
{
	my ($self) = @_;
	
	HTTP::Response->parse( *$self->{response} );
}

1;
